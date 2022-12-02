package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.UserOnlineDTO;
import org.cuit.epoch.entity.UserInfo;
import org.cuit.epoch.vo.*;
import org.cuit.epoch.vo.page.PageResult;
import org.springframework.web.multipart.MultipartFile;

/**
 * @author: ladidol
 * @date: 2022/11/20 21:30
 * @description:
 */
public interface UserInfoService extends IService<UserInfo> {

    /**
     * 修改用户资料
     *
     * @param userInfoVO 用户资料
     */
    void updateUserInfo(UserInfoVO userInfoVO);

    /**
     * 修改用户头像
     *
     * @param file 头像图片
     * @return 头像地址
     */
    String updateUserAvatar(MultipartFile file);

    /**
     * 绑定用户邮箱
     *
     * @param emailVO 邮箱
     */
    void saveUserEmail(EmailVO emailVO);

    /**
     * 更新用户角色+昵称
     *
     * @param userRoleVO 更新用户角色+昵称
     */
    void updateUserRole(UserRoleVO userRoleVO);

    /**
     * 修改用户禁用状态
     *
     * @param userDisableVO 用户禁用信息
     */
    void updateUserDisable(UserDisableVO userDisableVO);

    /**
     * 查看在线用户列表
     *
     * @param conditionVO 条件
     * @return 在线用户列表
     */
    PageResult<UserOnlineDTO> listOnlineUsers(ConditionVO conditionVO);

    /**
     * 下线用户
     *
     * @param userInfoId 用户信息id
     */
    void removeOnlineUser(Integer userInfoId);

}
