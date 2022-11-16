package org.cuit.epoch.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Param;
import org.cuit.epoch.entity.UserAuth;
import org.springframework.stereotype.Repository;

import java.util.List;

@Mapper
public interface UserAuthMapper extends BaseMapper<UserAuth> {

//    /**
//     * 查询后台用户列表
//     *
//     * @param current   页码
//     * @param size      大小
//     * @param condition 条件
//     * @return {@link List <UserBackDTO>} 用户列表
//     */
//    List<UserBackDTO> listUsers(@Param("current") Long current, @Param("size") Long size, @Param("condition") ConditionVO condition);
//
//    /**
//     * 查询后台用户数量
//     *
//     * @param condition 条件
//     * @return 用户数量
//     */
//    Integer countUser(@Param("condition") ConditionVO condition);

}
